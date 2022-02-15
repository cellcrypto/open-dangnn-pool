import Ember from 'ember';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),
  controller: Ember.inject.controller('login'),

  // init() {
  //   this.get('auth').clearIdToken()
  // },
  beforeModel() {
    this.get('auth').clearIdToken();
  },
  
  actions: {
    authenticate() {
      const { login, password } = this.get('controller').getProperties('login', 'password');
      this.get('auth').login(login, password).then(() => {
        // alert('Success! Click the top link!');
        this.transitionTo('/');
      }, (err) => {
        alert('Login Error: ' + err);
      });
    }
  }
});
