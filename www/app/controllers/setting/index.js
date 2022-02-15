import Ember from 'ember';

export default Ember.Controller.extend({
  cachedDevId: Ember.computed('devid', {
    get() {
      return localStorage.getItem('devid');
    },
    // set(key, value) {
    //   //localStorage.setItem('devid',value)
    //   return value;
    // }
  }),
  
});